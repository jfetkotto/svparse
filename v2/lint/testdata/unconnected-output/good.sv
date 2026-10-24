module leaf (input logic clk, input logic d, output logic q, output logic dbg);
  assign q = d & clk;
  assign dbg = d;
endmodule
module top (input logic clk, input logic d, output logic q, output logic dbg, input logic a);
  logic y, z;
  leaf u0 (.clk(clk), .d(a), .q(y), .dbg());      // open on purpose
  leaf u1 (.*);
  leaf u2 (.clk, .d(a), .q(z), .dbg());
  leaf u3 (clk, a, y, z);
endmodule
module pad (inout wire io, input logic oe, output logic c);
  assign c = io;
endmodule
module pads (input logic oe, output logic c);
  wire a;
  pad p0 (.io(a), .oe(oe), .c(c));
  pad p1 (.io(), .oe(oe), .c(c));
endmodule
