module leaf (input logic clk, input logic d, output logic q, output logic dbg);
  assign q = d & clk;
  assign dbg = d;
endmodule
module top (input logic clk, input logic a, output logic y);
  leaf u0 (.clk(clk), .d(a), .q(y));            // want
  leaf u1 (clk, a, y);                          // want
  leaf u2 (.clk(clk), .d(a), .dbg());           // want
  leaf u_arr [1:0] (.clk(clk), .d(a), .dbg());  // want
endmodule
