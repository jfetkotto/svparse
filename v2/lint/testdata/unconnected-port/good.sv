module leaf (input logic clk, input logic d, output logic q, output logic dbg);
  assign q = d & clk;
  assign dbg = d;
endmodule
module top (input logic clk, input logic d, output logic q, output logic dbg, input logic a);
  logic y, z;
  leaf u0 (.clk(clk), .d(a), .q(y), .dbg());      // open on purpose
  leaf u1 (.*);                                    // connects by name
  leaf u2 (.clk, .d(a), .q(z), .dbg());
  leaf u3 (clk, a, y, z);
  for (genvar i = 0; i < 2; i++) begin : g
    logic o1, o2;
    leaf u (.clk(clk), .d(a), .q(o1), .dbg(o2));
  end
endmodule
