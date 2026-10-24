module leaf (input logic clk, input logic d, output logic q);
  assign q = d & clk;
endmodule
module top (input logic clk, input logic d, output logic q, input logic a);
  logic y, z;
  leaf u0 (.clk(clk), .d(a), .q());
  leaf u1 (.*);
  leaf u2 (.clk, .d(a), .q(z));
  leaf u3 (clk, a, y);
  for (genvar i = 0; i < 2; i++) begin : g
    logic o;
    leaf u (.clk(clk), .d(a), .q(o));
  end
endmodule
